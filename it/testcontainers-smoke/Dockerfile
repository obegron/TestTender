FROM maven:3.9.11-eclipse-temurin-17

WORKDIR /workspace

COPY pom.xml ./
RUN mvn -q -DskipTests dependency:go-offline

COPY src ./src
RUN mvn -q dependency:get \
      -Dartifact=org.apache.maven.surefire:surefire-junit-platform:3.5.4

ENV TESTCONTAINERS_RYUK_DISABLED=true
ENV TESTCONTAINERS_CHECKS_DISABLE=true

CMD ["mvn", "-q", "test"]
